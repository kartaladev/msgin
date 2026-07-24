package msghttp_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// TestEncodeSSEEvent covers the encode-side framing rules (Task 1 hot-path
// branches 1, 2, 3, 3b and 4): the golden success frames and the
// CR/LF/NUL-in-ID/Name rejection class share the same call shape
// (EncodeSSEEvent(buf, ev)) and are folded into one table per the table-test
// skill.
func TestEncodeSSEEvent(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		ev     msghttp.SSEEvent
		assert func(t *testing.T, written string, err error)
	}

	cases := []testCase{
		{
			// Branch 1: full event, id+event+3-line data -> exact frame
			// bytes, "data: " = colon + exactly one space, per line.
			name: "full event: id, name, and 3-line data",
			ev:   msghttp.SSEEvent{ID: "42", Name: "update", Data: []byte("line1\nline2\nline3")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "id: 42\nevent: update\ndata: line1\ndata: line2\ndata: line3\n\n", written)
			},
		},
		{
			// Branch 2: ID=="" -> the id: line is omitted entirely.
			name: "empty ID omits the id line",
			ev:   msghttp.SSEEvent{Name: "update", Data: []byte("hello")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "event: update\ndata: hello\n\n", written)
			},
		},
		{
			// Branch 2: Name=="" -> the event: line is omitted entirely.
			name: "empty Name omits the event line",
			ev:   msghttp.SSEEvent{ID: "42", Data: []byte("hello")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "id: 42\ndata: hello\n\n", written)
			},
		},
		{
			// Branch 2: both empty -> a bare data-only frame.
			name: "empty ID and Name omit both lines",
			ev:   msghttp.SSEEvent{Data: []byte("hello")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "data: hello\n\n", written)
			},
		},
		{
			// Branch 3: empty (non-nil) Data still emits one uniform
			// "data: \n" line so the event is still dispatched.
			name: "empty Data emits a single data: line",
			ev:   msghttp.SSEEvent{ID: "1", Data: []byte{}},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "id: 1\ndata: \n\n", written)
			},
		},
		{
			// Branch 3: nil Data behaves identically to empty Data.
			name: "nil Data emits a single data: line",
			ev:   msghttp.SSEEvent{ID: "1", Data: nil},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "id: 1\ndata: \n\n", written)
			},
		},
		{
			// Branch 3b (F5/MINOR-1): a payload whose own first byte is a
			// space survives round-trippably as two spaces: the uniform
			// framing space, then the payload's own.
			name: "payload beginning with a space keeps its own space",
			ev:   msghttp.SSEEvent{Data: []byte(" hello")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "data:  hello\n\n", written)
			},
		},
		{
			// Data CR/LF/CRLF normalization (whole-branch review C1): a bare
			// CR in Data is a line boundary, framed as its own data: line —
			// never written raw, where a WHATWG parser would reinterpret it as
			// a field boundary and forge events. "a\rb" -> two data: lines.
			name: "CR in Data becomes a data-line boundary",
			ev:   msghttp.SSEEvent{Data: []byte("a\rb")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "data: a\ndata: b\n\n", written)
				assert.NotContains(t, written, "\r", "no bare CR may survive on the wire")
			},
		},
		{
			// CRLF in Data normalizes to a single line boundary (not two): the
			// \r\n pair is one terminator, exactly as SSE defines it.
			name: "CRLF in Data becomes a single data-line boundary",
			ev:   msghttp.SSEEvent{Data: []byte("a\r\nb")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "data: a\ndata: b\n\n", written)
				assert.NotContains(t, written, "\r", "no bare CR may survive on the wire")
			},
		},
		{
			// Two consecutive bare CRs are two line boundaries, yielding an
			// empty middle data: line — the same as "a\n\nb" would.
			name: "consecutive CRs in Data yield an empty middle data line",
			ev:   msghttp.SSEEvent{Data: []byte("a\r\rb")},
			assert: func(t *testing.T, written string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "data: a\ndata: \ndata: b\n\n", written)
				assert.NotContains(t, written, "\r", "no bare CR may survive on the wire")
			},
		},
		{
			// Branch 4: CR in ID -> ErrInvalidEventField, zero bytes written.
			name: "CR in ID is rejected",
			ev:   msghttp.SSEEvent{ID: "bad\rid", Data: []byte("x")},
			assert: func(t *testing.T, written string, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidEventField)
				assert.Empty(t, written)
			},
		},
		{
			// Branch 4: LF in ID -> ErrInvalidEventField, zero bytes written.
			name: "LF in ID is rejected",
			ev:   msghttp.SSEEvent{ID: "bad\nid", Data: []byte("x")},
			assert: func(t *testing.T, written string, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidEventField)
				assert.Empty(t, written)
			},
		},
		{
			// Branch 4: NUL in ID -> ErrInvalidEventField, zero bytes written.
			name: "NUL in ID is rejected",
			ev:   msghttp.SSEEvent{ID: "bad\x00id", Data: []byte("x")},
			assert: func(t *testing.T, written string, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidEventField)
				assert.Empty(t, written)
			},
		},
		{
			// Branch 4: CR in Name -> ErrInvalidEventField, zero bytes written.
			name: "CR in Name is rejected",
			ev:   msghttp.SSEEvent{Name: "bad\rname", Data: []byte("x")},
			assert: func(t *testing.T, written string, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidEventField)
				assert.Empty(t, written)
			},
		},
		{
			// Branch 4: LF in Name -> ErrInvalidEventField, zero bytes written.
			name: "LF in Name is rejected",
			ev:   msghttp.SSEEvent{Name: "bad\nname", Data: []byte("x")},
			assert: func(t *testing.T, written string, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidEventField)
				assert.Empty(t, written)
			},
		},
		{
			// Branch 4: NUL in Name -> ErrInvalidEventField, zero bytes written.
			name: "NUL in Name is rejected",
			ev:   msghttp.SSEEvent{Name: "bad\x00name", Data: []byte("x")},
			assert: func(t *testing.T, written string, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidEventField)
				assert.Empty(t, written)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			err := msghttp.EncodeSSEEvent(&buf, tc.ev)
			tc.assert(t, buf.String(), err)
		})
	}
}

// errFakeWrite is the sentinel failingSSEWriter always fails with.
var errFakeWrite = errors.New("failingSSEWriter: connection reset by peer")

// failingSSEWriter is an io.Writer whose Write always fails, standing in for
// a client that hangs up mid-stream. It is a genuinely different test double
// from the bytes.Buffer used throughout TestEncodeSSEEvent (a mock vs. a
// plain byte sink), which is why branch 5 gets its own test rather than a row
// in that table (table-test skill's "structurally different setup" carve-out).
type failingSSEWriter struct{}

func (failingSSEWriter) Write([]byte) (int, error) {
	return 0, errFakeWrite
}

// TestEncodeSSEEvent_writerErrorReturnedRaw covers hot-path branch 5: a write
// failure is returned exactly as the writer produced it, with no wrapping.
func TestEncodeSSEEvent_writerErrorReturnedRaw(t *testing.T) {
	t.Parallel()

	err := msghttp.EncodeSSEEvent(failingSSEWriter{}, msghttp.SSEEvent{Data: []byte("hello")})

	require.Error(t, err)
	assert.Same(t, errFakeWrite, err, "a writer error must be returned raw, not wrapped")
}

// TestSSEEventFromMessage covers hot-path branches 6, 7, 8 and 9: they all
// share the same call shape, SSEEventFromMessage(msg, cfg), so they are
// folded into one table per the table-test skill.
func TestSSEEventFromMessage(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		msg    func() msgin.Message[any]
		cfg    func() *msghttp.Config
		assert func(t *testing.T, ev msghttp.SSEEvent, err error)
	}

	cases := []testCase{
		{
			// Branch 6: []byte payload -> Data verbatim.
			name: "[]byte payload becomes Data verbatim",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("hello"))
			},
			assert: func(t *testing.T, ev msghttp.SSEEvent, err error) {
				require.NoError(t, err)
				assert.Equal(t, []byte("hello"), ev.Data)
			},
		},
		{
			// Branch 6: string payload -> Data converted.
			name: "string payload becomes Data as bytes",
			msg: func() msgin.Message[any] {
				return msgin.New[any]("hello-string")
			},
			assert: func(t *testing.T, ev msghttp.SSEEvent, err error) {
				require.NoError(t, err)
				assert.Equal(t, []byte("hello-string"), ev.Data)
			},
		},
		{
			// Branch 6: any other payload type -> ErrUnsupportedPayload.
			name: "non-bytes payload is unsupported",
			msg: func() msgin.Message[any] {
				return msgin.New[any](42)
			},
			assert: func(t *testing.T, _ msghttp.SSEEvent, err error) {
				assert.ErrorIs(t, err, msghttp.ErrUnsupportedPayload)
			},
		},
		{
			// Branch 7: the HeaderSSEEventName header wins over WithEventName.
			name: "header event name wins over WithEventName default",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader(msghttp.HeaderSSEEventName, "header-event")
			},
			cfg: func() *msghttp.Config {
				cfg, err := msghttp.NewConfig(msghttp.WithEventName("default-event"))
				require.NoError(t, err)
				return cfg
			},
			assert: func(t *testing.T, ev msghttp.SSEEvent, err error) {
				require.NoError(t, err)
				assert.Equal(t, "header-event", ev.Name)
			},
		},
		{
			// Branch 7: absent header falls back to WithEventName.
			name: "absent header falls back to WithEventName default",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x"))
			},
			cfg: func() *msghttp.Config {
				cfg, err := msghttp.NewConfig(msghttp.WithEventName("default-event"))
				require.NoError(t, err)
				return cfg
			},
			assert: func(t *testing.T, ev msghttp.SSEEvent, err error) {
				require.NoError(t, err)
				assert.Equal(t, "default-event", ev.Name)
			},
		},
		{
			// Branch 7: both absent resolves to "".
			name: "both header and cfg default absent resolves to empty name",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x"))
			},
			assert: func(t *testing.T, ev msghttp.SSEEvent, err error) {
				require.NoError(t, err)
				assert.Empty(t, ev.Name)
			},
		},
		{
			// Branch 8 (Config nil-safety contract): cfg == nil still works,
			// name resolves to "".
			name: "nil cfg works, name resolves to empty",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x"))
			},
			cfg: func() *msghttp.Config { return nil },
			assert: func(t *testing.T, ev msghttp.SSEEvent, err error) {
				require.NoError(t, err)
				assert.Empty(t, ev.Name)
			},
		},
		{
			// Branch 8: a hand-built zero-value &Config{} also works.
			name: "hand-built zero-value Config works, name resolves to empty",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x"))
			},
			cfg: func() *msghttp.Config { return &msghttp.Config{} },
			assert: func(t *testing.T, ev msghttp.SSEEvent, err error) {
				require.NoError(t, err)
				assert.Empty(t, ev.Name)
			},
		},
		{
			// Branch 9: a hostile header value (as a reconstructed message
			// via msgin.NewMessage could carry) is rejected.
			name: "hostile header value is rejected",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader(msghttp.HeaderSSEEventName, "evil\nevent: forged")
			},
			assert: func(t *testing.T, _ msghttp.SSEEvent, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidEventField)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var cfg *msghttp.Config
			if tc.cfg != nil {
				cfg = tc.cfg()
			}

			ev, err := msghttp.SSEEventFromMessage(tc.msg(), cfg)
			tc.assert(t, ev, err)
		})
	}
}

// mustEncodeSSE encodes ev via msghttp.EncodeSSEEvent (Task 1) and returns
// the resulting wire bytes as a string, for the RT (round-trip) table rows
// below — it fails the test immediately if the encode side rejects ev
// (which none of the RT inputs used here do).
func mustEncodeSSE(t *testing.T, ev msghttp.SSEEvent) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, msghttp.EncodeSSEEvent(&buf, ev))
	return buf.String()
}

// buildSSEDataLines returns a sequence of raw "data: <value>\n" lines whose
// combined contribution to the parser's data buffer (each line's value bytes
// plus its '\n') sums to EXACTLY totalBytes, chunked into <=512 KiB values so
// no single line's own byte count approaches totalBytes (the line-byte
// counter must never be the one that trips for these two rows — only the
// data-buffer counter is under test).
func buildSSEDataLines(totalBytes int64) string {
	const chunk = 512 * 1024

	var b strings.Builder
	remaining := totalBytes
	for remaining > 0 {
		n := remaining
		if n > chunk {
			n = chunk
		}
		valLen := n - 1 // this line contributes exactly n bytes: valLen value bytes + 1 '\n'
		b.WriteString("data: ")
		b.WriteString(strings.Repeat("a", int(valLen)))
		b.WriteByte('\n')
		remaining -= n
	}
	return b.String()
}

// TestSSEParser_Next covers Task 2's WHATWG text/event-stream processing
// rules and the INV-S6/MAJOR-4/F4 per-event cap: every case shares the same
// call shape (build a parser over a raw wire stream, then drive and inspect
// it via the assert closure, which may call Next/RetryDelay/LastEventID as
// many times as the rule under test needs) per the table-test skill.
func TestSSEParser_Next(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		input  string
		opts   []msghttp.Option
		reader func() io.Reader // overrides the default strings.NewReader(input) when non-nil
		assert func(t *testing.T, p *msghttp.SSEParser)
	}

	cases := []testCase{
		{
			// BOM: a single leading U+FEFF is stripped once at stream start
			// and never becomes part of the first field/line.
			name:  "leading BOM is stripped once",
			input: "\xEF\xBB\xBFdata: hello\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("hello"), ev.Data)
				assert.Empty(t, ev.Name)
				assert.Empty(t, ev.ID)
			},
		},
		{
			// LF line ending; also the base "data-append" case (a single
			// data: line becomes Data verbatim).
			name:  "LF line ending / single data line append",
			input: "data: hello\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("hello"), ev.Data)
			},
		},
		{
			// CRLF line ending: the \r\n pair is consumed as ONE line
			// ending, not two (a bare \n right after would otherwise look
			// like an extra blank line).
			name:  "CRLF line ending",
			input: "data: hello\r\n\r\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("hello"), ev.Data)
			},
		},
		{
			// Bare CR (not followed by LF) ends a line too, including the
			// terminating blank line; this also exercises multi-data join
			// (two data: lines joined with '\n').
			name:  "bare CR line ending, multi-data join",
			input: "data: hello\rdata: world\r\r",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("hello\nworld"), ev.Data)
			},
		},
		{
			// Comment: a line starting with ':' is ignored outright — never
			// becomes a field, never touches the data buffer.
			name:  "comment line is ignored",
			input: ": this is a comment\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("x"), ev.Data)
			},
		},
		{
			// space-strip: no leading space in the value -> nothing to strip.
			name:  "field value with no leading space is unchanged",
			input: "data:x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("x"), ev.Data)
			},
		},
		{
			// space-strip: exactly one leading space of the value is
			// stripped.
			name:  "field value with one leading space has it stripped",
			input: "data: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("x"), ev.Data)
			},
		},
		{
			// space-strip: only ONE leading space is stripped; a second one
			// is the value's own and survives.
			name:  "field value with two leading spaces keeps one",
			input: "data:  x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte(" x"), ev.Data)
			},
		},
		{
			// no-colon: a line with no ':' is the whole field name, value
			// "" — here "data" with an empty value still triggers dispatch
			// (the data buffer becomes non-empty: just the '\n').
			name:  "line with no colon is the field name, empty value",
			input: "data\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Empty(t, ev.Data)
			},
		},
		{
			// event: sets the type buffer, becomes SSEEvent.Name.
			name:  "event field sets Name",
			input: "event: update\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, "update", ev.Name)
				assert.Equal(t, []byte("x"), ev.Data)
			},
		},
		{
			// id: sets the id buffer, becomes SSEEvent.ID and LastEventID().
			name:  "id field sets ID and LastEventID",
			input: "id: 42\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, "42", ev.ID)
				assert.Equal(t, "42", p.LastEventID())
			},
		},
		{
			// id+NUL: an id: value containing NUL is ignored entirely (the
			// id buffer, empty before this event, stays empty).
			name:  "id field containing NUL is ignored",
			input: "id: bad\x00id\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Empty(t, ev.ID)
				assert.Empty(t, p.LastEventID())
			},
		},
		{
			// empty id: clears a previously-set id buffer (does not just
			// leave it unset — it actively resets it).
			name:  "empty id field clears a previously-set id",
			input: "id: 1\ndata: a\n\nid:\ndata: b\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev1, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, "1", ev1.ID)

				ev2, err := p.Next()
				require.NoError(t, err)
				assert.Empty(t, ev2.ID)
				assert.Empty(t, p.LastEventID())
			},
		},
		{
			// retry digits -> RetryDelay in milliseconds.
			name:  "retry field with ASCII digits sets RetryDelay",
			input: "retry: 500\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				require.NoError(t, err)
				d, ok := p.RetryDelay()
				assert.True(t, ok)
				assert.Equal(t, 500*time.Millisecond, d)
			},
		},
		{
			// retry non-digit is ignored, AND the retry value persists
			// across events (stream-level state, not reset per dispatch) —
			// both proven by the second event's invalid retry leaving the
			// first event's valid one intact.
			name:  "retry field with non-digit value is ignored, prior value persists",
			input: "retry: 500\ndata: a\n\nretry: not-a-number\ndata: b\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				require.NoError(t, err)
				d, ok := p.RetryDelay()
				require.True(t, ok)
				require.Equal(t, 500*time.Millisecond, d)

				_, err = p.Next()
				require.NoError(t, err)
				d, ok = p.RetryDelay()
				assert.True(t, ok)
				assert.Equal(t, 500*time.Millisecond, d, "an invalid retry: must not clobber the prior valid value")
			},
		},
		{
			// retry: an empty value is not ASCII-digits-only either -
			// ignored, RetryDelay stays unset.
			name:  "retry field with empty value is ignored",
			input: "retry:\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				require.NoError(t, err)
				_, ok := p.RetryDelay()
				assert.False(t, ok)
			},
		},
		{
			// retry: an all-digits value that overflows int64 is ignored
			// (strconv.ParseInt's error arm), not clamped or truncated.
			name:  "retry field value overflowing int64 is ignored",
			input: "retry: 99999999999999999999\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				require.NoError(t, err)
				_, ok := p.RetryDelay()
				assert.False(t, ok)
			},
		},
		{
			// unknown field: ignored outright — no error, no effect on
			// Name/ID/RetryDelay.
			name:  "unrecognized field name is ignored",
			input: "foo: bar\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("x"), ev.Data)
				assert.Empty(t, ev.Name)
				assert.Empty(t, ev.ID)
				_, ok := p.RetryDelay()
				assert.False(t, ok)
			},
		},
		{
			// empty-data no-dispatch: a blank line with an empty data
			// buffer dispatches NOTHING, and the type buffer set by the
			// event: field IS reset — proven because the next real
			// dispatch's Name comes back empty despite the earlier
			// "event: foo".
			name:  "blank line with empty data buffer does not dispatch, resets type buffer",
			input: "event: foo\n\ndata: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Empty(t, ev.Name, "type buffer must have been reset by the no-op blank line")
				assert.Equal(t, []byte("x"), ev.Data)
			},
		},
		{
			// id persistence across events: an id set on one event carries
			// forward into the next event that has no id: field of its own.
			name:  "id persists across events without their own id field",
			input: "id: 1\ndata: a\n\ndata: b\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev1, err := p.Next()
				require.NoError(t, err)
				require.Equal(t, "1", ev1.ID)

				ev2, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, "1", ev2.ID)
				assert.Equal(t, "1", p.LastEventID())
			},
		},
		{
			// Clean EOF: between events (no pending line/data-buffer/type
			// state), Next reports io.EOF exactly, not ErrUnexpectedEOF.
			name:  "clean EOF between events",
			input: "data: x\n\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				require.NoError(t, err)

				ev, err := p.Next()
				assert.ErrorIs(t, err, io.EOF)
				assert.False(t, errors.Is(err, io.ErrUnexpectedEOF), "clean EOF must not also satisfy ErrUnexpectedEOF")
				assert.Equal(t, msghttp.SSEEvent{}, ev)
			},
		},
		{
			// Mid-event EOF, arm 1: the stream ends in the middle of an
			// unterminated line (no line ending was ever found) -> the
			// partial event is discarded, ErrUnexpectedEOF.
			name:  "mid-event EOF: unterminated final line",
			input: "data: partial",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
				assert.Equal(t, msghttp.SSEEvent{}, ev)
			},
		},
		{
			// Mid-event EOF, arm 2: the stream ends after a fully terminated
			// field line but with no blank line ever arriving to dispatch
			// it -> the partial event is discarded, ErrUnexpectedEOF. This
			// is a DIFFERENT code path from arm 1 (readLine itself returns
			// io.EOF cleanly here; the pending-state check is what turns it
			// into ErrUnexpectedEOF).
			name:  "mid-event EOF: no blank line ever arrives",
			input: "data: x\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
				assert.Equal(t, msghttp.SSEEvent{}, ev)
			},
		},
		{
			// Cap, at exactly the limit: a data buffer of EXACTLY
			// maxEventBytes bytes (5 lines, each contributing "a\n" == 2
			// bytes, cap 10) parses with no error — "exceeds" means
			// strictly greater than. Each individual wire line ("data: a",
			// 7 bytes) stays well under the cap, so it is the data-buffer
			// counter under test here, not the line counter.
			name:  "at cap: data buffer exactly at maxEventBytes parses",
			input: strings.Repeat("data: a\n", 5) + "\n",
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("a\na\na\na\na"), ev.Data)
			},
		},
		{
			// Over cap via the LINE counter: a single line with no
			// terminator that keeps growing past maxEventBytes trips
			// ErrEventTooLarge before a terminator is ever found.
			name:  "over cap: single unterminated line trips the line counter",
			input: "data:" + strings.Repeat("a", 40), // no trailing newline at all
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, msghttp.ErrEventTooLarge)
			},
		},
		{
			// Over cap via the DATA-BUFFER counter: three data: lines, each
			// individually well under the 10-byte cap (9 raw chars each),
			// whose cumulative data-buffer contribution (4 bytes each: 3
			// value + '\n') exceeds it on the third line.
			name:  "over cap: many data lines in one event trip the data-buffer counter",
			input: "data: abc\ndata: def\ndata: ghi\n\n",
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, msghttp.ErrEventTooLarge)
			},
		},
		{
			// Cap, at exactly the limit, LINE counter: a single raw line
			// of EXACTLY maxEventBytes bytes ("data:" + 5 chars = 10)
			// parses with no error — the parallel boundary case to "at
			// cap: data buffer exactly at maxEventBytes parses" above,
			// but for the line-byte counter in readLine rather than the
			// data-buffer counter in processField.
			name:  "at cap: single line exactly maxEventBytes bytes parses",
			input: "data:" + strings.Repeat("a", 5) + "\n\n",
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("aaaaa"), ev.Data)
			},
		},
		{
			// Over cap, LINE counter, one byte past the boundary above:
			// a raw line of maxEventBytes+1 bytes ("data:" + 6 chars =
			// 11) trips ErrEventTooLarge — "exceeds" means strictly
			// greater than, exactly like the data-buffer counter's
			// boundary.
			name:  "over cap: single line one byte over maxEventBytes trips the line counter",
			input: "data:" + strings.Repeat("a", 6) + "\n\n",
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, msghttp.ErrEventTooLarge)
			},
		},
		{
			// Regression (Important finding): a LINE-counter cap trip
			// must not leave stale dataBuf bytes from an
			// already-in-progress event to corrupt whatever dispatches
			// next, and must not leave the reader positioned mid-line.
			// "data: ab" starts accumulating an event's data buffer;
			// the following "data:" + 10 'c's then trips the line
			// counter (cap 10) before its own terminator is reached.
			// Contract A (continue-past-oversized): the abandoned
			// line's remaining bytes are drained to its terminator, the
			// event buffers are reset, and a SUBSEQUENT Next() call
			// resumes cleanly at the next real event ("ok") — not the
			// stale "ab", and not a re-trip on the drained garbage.
			name:  "line-counter cap trip resets buffers and drains to the next event",
			input: "data: ab\n" + "data:" + strings.Repeat("c", 10) + "\n" + "\n" + "data: ok\n" + "\n",
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, msghttp.ErrEventTooLarge)

				ev, err := p.Next()
				require.NoError(t, err, "a subsequent Next() must resume cleanly at the next event, not re-trip on drained garbage")
				assert.Equal(t, []byte("ok"), ev.Data, "the discarded event's stale data ('ab') must not leak into the next dispatched event")
			},
		},
		{
			// Same drain-and-continue contract as the row above, but the
			// abandoned oversized line ends in a CRLF rather than a bare
			// LF: the drain must consume the paired CR+LF as ONE line
			// ending (matching readLine's own CRLF handling), not leave
			// the LF behind to be misread as an extra blank line.
			name:  "line-counter cap trip drains a CRLF-terminated abandoned line",
			input: "data: ab\n" + "data:" + strings.Repeat("c", 10) + "\r\n" + "\n" + "data: ok\n" + "\n",
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, msghttp.ErrEventTooLarge)

				ev, err := p.Next()
				require.NoError(t, err, "a subsequent Next() must resume cleanly at the next event, not re-trip on drained garbage")
				assert.Equal(t, []byte("ok"), ev.Data, "the discarded event's stale data ('ab') must not leak into the next dispatched event")
			},
		},
		{
			// Same drain-and-continue contract as the two rows above,
			// but the abandoned oversized line ends in a bare CR (a
			// '\r' NOT followed by '\n'): drainLine's CR branch must
			// stop at the CR itself (its Peek(1) sees "data:" next, not
			// a paired LF) rather than consuming into the following
			// line, so the subsequent "data: ok" line is left intact
			// for the next Next() call to dispatch.
			name:  "line-counter cap trip drains a bare-CR-terminated abandoned line",
			input: "data: ab\n" + "data:" + strings.Repeat("c", 10) + "\r" + "data: ok\n" + "\n",
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, msghttp.ErrEventTooLarge)

				ev, err := p.Next()
				require.NoError(t, err, "a subsequent Next() must resume cleanly at the next event, not re-trip on drained garbage")
				assert.Equal(t, []byte("ok"), ev.Data, "the discarded event's stale data ('ab') must not leak into the next dispatched event")

				_, err = p.Next()
				assert.ErrorIs(t, err, io.EOF)
			},
		},
		{
			// Cap does NOT false-trip on a run of small, complete,
			// blank-line-separated events whose CUMULATIVE bytes across the
			// whole stream exceed the cap, because the data buffer resets
			// at every dispatch.
			name:  "cap does not false-trip on many small complete events",
			input: strings.Repeat("data: ab\n\n", 5), // 5 events, "ab\n" = 3 bytes each, cap 10
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				for range 5 {
					ev, err := p.Next()
					require.NoError(t, err)
					assert.Equal(t, []byte("ab"), ev.Data)
				}
				_, err := p.Next()
				assert.ErrorIs(t, err, io.EOF)
			},
		},
		{
			// Cap does NOT false-trip on a run of bare comment lines (no
			// blank line between them) whose CUMULATIVE bytes exceed the
			// cap, because a comment line never touches the data buffer and
			// the line counter resets every line.
			name:  "cap does not false-trip on a run of bare comments",
			input: strings.Repeat(": ping\n", 50), // each line 6 bytes, cap 10, 50 of them = 300 bytes cumulative
			opts:  []msghttp.Option{msghttp.WithMaxEventBytes(10)},
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, io.EOF)
			},
		},
		{
			// RT (round-trip), discharging Task 1 branch 3b / MINOR-1: a
			// payload beginning with a space survives EncodeSSEEvent ->
			// NewSSEParser byte-exact. EncodeSSEEvent's uniform framing
			// space plus the parser's single-leading-space strip cancel out
			// exactly.
			name:  "RT: payload beginning with a space round-trips byte-exact",
			input: mustEncodeSSE(t, msghttp.SSEEvent{Data: []byte(" hello")}),
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte(" hello"), ev.Data)
			},
		},
		{
			// RT (whole-branch review C1): a payload carrying a bare CR
			// round-trips to LF — the CR is an SSE line terminator, never
			// data content, so "a\rb" legitimately encodes to two data: lines
			// and parses back to "a\nb". No bare CR reaches a subscriber.
			name:  "RT: payload with a bare CR round-trips to LF",
			input: mustEncodeSSE(t, msghttp.SSEEvent{Data: []byte("a\rb")}),
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("a\nb"), ev.Data)
			},
		},
		{
			// RT: a CRLF payload normalizes to a single LF the same way.
			name:  "RT: payload with a CRLF round-trips to a single LF",
			input: mustEncodeSSE(t, msghttp.SSEEvent{Data: []byte("a\r\nb")}),
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Equal(t, []byte("a\nb"), ev.Data)
			},
		},
		{
			// RT: an empty payload round-trips byte-exact too.
			name:  "RT: empty payload round-trips byte-exact",
			input: mustEncodeSSE(t, msghttp.SSEEvent{Data: []byte("")}),
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Empty(t, ev.Data)
			},
		},
		{
			// Default cap, proven behaviorally (not by reading the field):
			// an event whose data buffer is EXACTLY the 1 MiB default
			// parses with no error, WithMaxEventBytes left unset.
			name:  "default cap: data buffer exactly at the 1 MiB default parses",
			input: buildSSEDataLines(1<<20) + "\n",
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				ev, err := p.Next()
				require.NoError(t, err)
				assert.Len(t, ev.Data, (1<<20)-1) // cap bytes minus the stripped trailing '\n'
			},
		},
		{
			// Default cap: one byte more than the 1 MiB default errors.
			name:  "default cap: one byte over the 1 MiB default errors",
			input: buildSSEDataLines((1 << 20) + 1),
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.ErrorIs(t, err, msghttp.ErrEventTooLarge)
			},
		},
		{
			// A raw, non-EOF read error from the underlying reader is
			// returned exactly as produced, unwrapped — the same "read
			// errors returned raw" contract as EncodeSSEEvent's writer
			// error (Task 1 branch 5).
			name:   "raw read error is returned unwrapped",
			reader: func() io.Reader { return &failingSSEReader{prefix: []byte("data: x")} },
			assert: func(t *testing.T, p *msghttp.SSEParser) {
				_, err := p.Next()
				assert.Same(t, errFakeSSERead, err, "a reader error must be returned raw, not wrapped")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var r io.Reader = strings.NewReader(tc.input)
			if tc.reader != nil {
				r = tc.reader()
			}

			p, err := msghttp.NewSSEParser(r, tc.opts...)
			require.NoError(t, err)
			tc.assert(t, p)
		})
	}
}

// errFakeSSERead is the sentinel failingSSEReader returns once its prefix is
// exhausted, standing in for a raw transport failure mid-stream.
var errFakeSSERead = errors.New("failingSSEReader: connection reset by peer")

// failingSSEReader is an io.Reader that emits prefix byte-by-byte and then
// always fails with errFakeSSERead — a genuinely different test double from
// strings.Reader (a mock vs. a plain byte source), which is why it gets its
// own type rather than a table row built on strings.NewReader.
type failingSSEReader struct {
	prefix []byte
}

func (r *failingSSEReader) Read(p []byte) (int, error) {
	if len(r.prefix) > 0 {
		n := copy(p, r.prefix)
		r.prefix = r.prefix[n:]
		return n, nil
	}
	return 0, errFakeSSERead
}

// TestNewSSEParser_maxEventBytesValidation covers WithMaxEventBytes'
// construction-time validation: the set-flag pattern distinguishes an
// explicit non-positive value (rejected) from unset (default) or an
// explicit valid boundary value (accepted).
func TestNewSSEParser_maxEventBytesValidation(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   []msghttp.Option
		assert func(t *testing.T, p *msghttp.SSEParser, err error)
	}

	cases := []testCase{
		{
			name: "unset takes the default, construction succeeds",
			assert: func(t *testing.T, p *msghttp.SSEParser, err error) {
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			name: "explicit zero is rejected",
			opts: []msghttp.Option{msghttp.WithMaxEventBytes(0)},
			assert: func(t *testing.T, p *msghttp.SSEParser, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxEventBytes)
				assert.Nil(t, p)
			},
		},
		{
			name: "explicit negative is rejected",
			opts: []msghttp.Option{msghttp.WithMaxEventBytes(-1)},
			assert: func(t *testing.T, p *msghttp.SSEParser, err error) {
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxEventBytes)
				assert.Nil(t, p)
			},
		},
		{
			name: "explicit positive boundary value is accepted",
			opts: []msghttp.Option{msghttp.WithMaxEventBytes(1)},
			assert: func(t *testing.T, p *msghttp.SSEParser, err error) {
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, err := msghttp.NewSSEParser(strings.NewReader("data: x\n\n"), tc.opts...)
			tc.assert(t, p, err)
		})
	}
}

// FuzzSSEParser asserts the parser's invariants hold over arbitrary byte
// input: it never panics, it never returns an event whose accounted Data
// size exceeds the configured cap, and it always terminates on a finite
// reader (a hard iteration ceiling here is a defensive circuit breaker, not
// an expected outcome — a genuinely non-terminating parser is itself a bug
// this fuzz target exists to catch).
func FuzzSSEParser(f *testing.F) {
	seeds := []string{
		"data: hello\n\n",
		"id: 1\ndata: x\n\n",
		"event: foo\ndata: bar\n\n",
		": comment\ndata: x\n\n",
		"data:no-space\n\n",
		"data: a\r\ndata: b\r\n\r\n",
		"data: a\rdata: b\r\r",
		"retry: 500\ndata: x\n\n",
		"retry: not-a-number\ndata: x\n\n",
		"\xEF\xBB\xBFdata: bom\n\n",
		"id:\ndata: x\n\n",
		"unknown: field\ndata: x\n\n",
		"data: partial",
		"data: x\n",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		const maxEventBytes = 1024

		p, err := msghttp.NewSSEParser(strings.NewReader(input), msghttp.WithMaxEventBytes(maxEventBytes))
		require.NoError(t, err)

		const maxEvents = 10_000
		for range maxEvents {
			ev, err := p.Next()
			if err != nil {
				return
			}
			if int64(len(ev.Data)) > maxEventBytes {
				t.Fatalf("Next returned an event with %d Data bytes, exceeding the %d cap", len(ev.Data), maxEventBytes)
			}
		}
		t.Fatalf("parser did not terminate within %d events for input %q", maxEvents, input)
	})
}
