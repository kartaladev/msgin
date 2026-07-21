package msgin_test

import (
	"maps"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaders_TypedAccessors(t *testing.T) {
	h := msgin.NewHeaders(map[string]any{
		"s":                   "hello",
		"n":                   42,
		msgin.HeaderTimestamp: time.Unix(1000, 0),
	})

	tests := []struct {
		name   string
		assert func(t *testing.T, h msgin.Headers)
	}{
		{"string present", func(t *testing.T, h msgin.Headers) {
			v, ok := h.String("s")
			assert.True(t, ok)
			assert.Equal(t, "hello", v)
		}},
		{"int present", func(t *testing.T, h msgin.Headers) {
			v, ok := h.Int("n")
			assert.True(t, ok)
			assert.Equal(t, 42, v)
		}},
		{"time present", func(t *testing.T, h msgin.Headers) {
			v, ok := h.Time(msgin.HeaderTimestamp)
			assert.True(t, ok)
			assert.Equal(t, time.Unix(1000, 0), v)
		}},
		{"get found", func(t *testing.T, h msgin.Headers) {
			v, ok := h.Get("s")
			assert.True(t, ok)
			assert.Equal(t, "hello", v)
		}},
		{"missing key", func(t *testing.T, h msgin.Headers) {
			_, ok := h.Get("nope")
			assert.False(t, ok)
		}},
		{"wrong type returns false", func(t *testing.T, h msgin.Headers) {
			_, ok := h.Int("s")
			assert.False(t, ok)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t, h) })
	}
}

func TestHeaders_All(t *testing.T) {
	src := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	}
	h := msgin.NewHeaders(src)

	tests := []struct {
		name   string
		assert func(t *testing.T, h msgin.Headers)
	}{
		{"full iteration yields all pairs", func(t *testing.T, h msgin.Headers) {
			got := maps.Collect(h.All())
			assert.Equal(t, src, got)
		}},
		{"early break stops iteration without panicking", func(t *testing.T, h msgin.Headers) {
			seen := 0
			assert.NotPanics(t, func() {
				for range h.All() {
					seen++
					break
				}
			})
			assert.Equal(t, 1, seen)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t, h) })
	}
}

func TestHeaders_Immutable_AllDoesNotExposeBackingMap(t *testing.T) {
	src := map[string]any{"k": "v"}
	h := msgin.NewHeaders(src)
	// Mutating the source map must not affect the Headers snapshot.
	src["k"] = "mutated"
	v, ok := h.String("k")
	require.True(t, ok)
	assert.Equal(t, "v", v)
}

func TestNew_StampsIDAndTimestamp(t *testing.T) {
	clk := clockwork.NewFakeClockAt(time.Unix(500, 0))

	tests := []struct {
		name   string
		opts   []msgin.MessageOption
		assert func(t *testing.T, m msgin.Message[string])
	}{
		{"auto id is non-empty", []msgin.MessageOption{msgin.WithClock(clk)},
			func(t *testing.T, m msgin.Message[string]) {
				assert.NotEmpty(t, m.ID())
			}},
		{"explicit id", []msgin.MessageOption{msgin.WithClock(clk), msgin.WithID("abc")},
			func(t *testing.T, m msgin.Message[string]) {
				assert.Equal(t, "abc", m.ID())
			}},
		{"timestamp from clock", []msgin.MessageOption{msgin.WithClock(clk)},
			func(t *testing.T, m msgin.Message[string]) {
				ts, ok := m.Header(msgin.HeaderTimestamp)
				require.True(t, ok)
				assert.Equal(t, time.Unix(500, 0), ts)
			}},
		{"payload preserved", []msgin.MessageOption{msgin.WithClock(clk)},
			func(t *testing.T, m msgin.Message[string]) {
				assert.Equal(t, "body", m.Payload())
			}},
		{"nil clock is a no-op, not a panic", []msgin.MessageOption{msgin.WithClock(nil)},
			func(t *testing.T, m msgin.Message[string]) {
				_, ok := m.Header(msgin.HeaderTimestamp)
				assert.True(t, ok, "the real-clock default still stamps a timestamp")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := msgin.New("body", tc.opts...)
			tc.assert(t, m)
		})
	}
}

func TestMessage_WithHeader_CopyOnWrite(t *testing.T) {
	m := msgin.New("x", msgin.WithID("id1"))
	m2 := m.WithHeader("k", "v")

	_, ok := m.Header("k")
	assert.False(t, ok, "original must be unchanged")
	v, ok := m2.Header("k")
	assert.True(t, ok)
	assert.Equal(t, "v", v)
}

func TestMessageWithoutHeader(t *testing.T) {
	tests := []struct {
		name   string
		build  func() msgin.Message[int]
		key    string
		assert func(t *testing.T, orig, got msgin.Message[int])
	}{
		{
			name:  "removes present key, leaves original",
			build: func() msgin.Message[int] { return msgin.New(1, msgin.WithHeaders(map[string]any{"k": "v"})) },
			key:   "k",
			assert: func(t *testing.T, orig, got msgin.Message[int]) {
				if _, ok := got.Header("k"); ok {
					t.Fatal("want k removed from result")
				}
				if _, ok := orig.Header("k"); !ok {
					t.Fatal("want k retained on original (copy-on-write)")
				}
			},
		},
		{
			name:  "removing an absent key is a no-op equal-value copy",
			build: func() msgin.Message[int] { return msgin.New(1, msgin.WithHeaders(map[string]any{"k": "v"})) },
			key:   "absent",
			assert: func(t *testing.T, orig, got msgin.Message[int]) {
				assert.Equal(t, maps.Collect(orig.Headers().All()), maps.Collect(got.Headers().All()))
			},
		},
		{
			name:  "removing from an empty header set is safe",
			build: func() msgin.Message[int] { return msgin.Message[int]{} },
			key:   "k",
			assert: func(t *testing.T, orig, got msgin.Message[int]) {
				_, ok := got.Header("k")
				assert.False(t, ok)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := tt.build()
			var got msgin.Message[int]
			assert.NotPanics(t, func() { got = orig.WithoutHeader(tt.key) })
			tt.assert(t, orig, got)
		})
	}
}

func TestNew_WithHeaders(t *testing.T) {
	clk := clockwork.NewFakeClockAt(time.Unix(500, 0))
	m := msgin.New("body", msgin.WithClock(clk), msgin.WithID("explicit-id"), msgin.WithHeaders(map[string]any{
		"custom":              "v",
		msgin.HeaderID:        "attacker-id",
		msgin.HeaderTimestamp: time.Unix(1, 0),
	}))

	tests := []struct {
		name   string
		assert func(t *testing.T, m msgin.Message[string])
	}{
		{"custom header from WithHeaders is present", func(t *testing.T, m msgin.Message[string]) {
			v, ok := m.Header("custom")
			assert.True(t, ok)
			assert.Equal(t, "v", v)
		}},
		{"reserved id header is New's stamped value, not WithHeaders'", func(t *testing.T, m msgin.Message[string]) {
			assert.Equal(t, "explicit-id", m.ID())
		}},
		{"reserved timestamp header is New's stamped value, not WithHeaders'", func(t *testing.T, m msgin.Message[string]) {
			ts, ok := m.Header(msgin.HeaderTimestamp)
			require.True(t, ok)
			assert.Equal(t, time.Unix(500, 0), ts)
		}},
		{"Headers() accessor exposes the same custom and reserved data", func(t *testing.T, m msgin.Message[string]) {
			h := m.Headers()
			v, ok := h.String("custom")
			assert.True(t, ok)
			assert.Equal(t, "v", v)
			id, ok := h.String(msgin.HeaderID)
			assert.True(t, ok)
			assert.Equal(t, "explicit-id", id)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t, m) })
	}
}

func TestNewMessage_PreservesHeadersWithoutRestamping(t *testing.T) {
	t.Parallel()

	// A pre-built envelope as a wire inbound adapter would reconstruct it from
	// storage: an explicit id, timestamp, and custom key already present.
	ts := time.Unix(1000, 0)
	headers := msgin.NewHeaders(map[string]any{
		msgin.HeaderID:        "stored-id",
		msgin.HeaderTimestamp: ts,
		"x-custom":            "trace-42",
	})

	tests := []struct {
		name   string
		assert func(t *testing.T, m msgin.Message[string])
	}{
		{"id is the stored value, not re-stamped", func(t *testing.T, m msgin.Message[string]) {
			assert.Equal(t, "stored-id", m.ID())
		}},
		{"timestamp is the stored value, not re-stamped", func(t *testing.T, m msgin.Message[string]) {
			got, ok := m.Header(msgin.HeaderTimestamp)
			require.True(t, ok)
			assert.Equal(t, ts, got)
		}},
		{"custom header is preserved intact", func(t *testing.T, m msgin.Message[string]) {
			v, ok := m.Headers().String("x-custom")
			assert.True(t, ok)
			assert.Equal(t, "trace-42", v)
		}},
		{"payload is preserved", func(t *testing.T, m msgin.Message[string]) {
			assert.Equal(t, "body", m.Payload())
		}},
		{"exact header set matches the input (nothing added)", func(t *testing.T, m msgin.Message[string]) {
			assert.Equal(t, maps.Collect(headers.All()), maps.Collect(m.Headers().All()))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := msgin.NewMessage("body", headers)
			tc.assert(t, m)
		})
	}
}

// TestNewMessage_NoStampContrastWithNew pins the behavioral contrast between
// NewMessage (reconstruct verbatim, no stamping) and New (produce with a fresh
// stamped id): NewMessage given empty Headers carries NO msgin.id, whereas New
// always stamps one.
func TestNewMessage_NoStampContrastWithNew(t *testing.T) {
	t.Parallel()

	reconstructed := msgin.NewMessage("body", msgin.NewHeaders(nil))
	_, hasID := reconstructed.Header(msgin.HeaderID)
	assert.False(t, hasID, "NewMessage must not stamp msgin.id")
	_, hasTS := reconstructed.Header(msgin.HeaderTimestamp)
	assert.False(t, hasTS, "NewMessage must not stamp msgin.timestamp")

	produced := msgin.New("body")
	assert.NotEmpty(t, produced.ID(), "New always stamps a msgin.id for contrast")
}

func TestMessage_ZeroValue_WithHeader(t *testing.T) {
	var m msgin.Message[string]

	var m2 msgin.Message[string]
	assert.NotPanics(t, func() { m2 = m.WithHeader("k", "v") })

	v, ok := m2.Header("k")
	assert.True(t, ok)
	assert.Equal(t, "v", v)
}
