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

func TestMessage_ZeroValue_WithHeader(t *testing.T) {
	var m msgin.Message[string]

	var m2 msgin.Message[string]
	assert.NotPanics(t, func() { m2 = m.WithHeader("k", "v") })

	v, ok := m2.Header("k")
	assert.True(t, ok)
	assert.Equal(t, "v", v)
}
