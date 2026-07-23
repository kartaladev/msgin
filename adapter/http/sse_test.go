package msghttp_test

import (
	"bytes"
	"errors"
	"testing"

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
