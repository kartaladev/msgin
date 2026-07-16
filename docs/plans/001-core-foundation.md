# Core Foundation & In-Memory Transport — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a working, end-to-end in-process messaging system for `msgin`: the typed message envelope, the payload codec, the adapter SPI, the in-memory adapter, and a minimal producer/consumer runtime (streaming ingest + worker pool + basic Ack/Nack).

**Architecture:** Three layers (caller `Message[T]` → non-generic SPI over `Message[any]` → backend). The payload codec (`T`↔`[]byte`) lives in the runtime; the in-memory adapter carries the *live* Go value with no codec (zero-copy). This plan builds everything except retry/dead-letter (Plan 002) and flow control (Plan 003) — a `Nack` here simply requeues.

**Tech Stack:** Go 1.25 (generics, `iter.Seq2`), `github.com/jonboulle/clockwork` (injectable time), `go.uber.org/goleak` (test-only), `github.com/stretchr/testify` (test-only), `go.uber.org/mock` (test-only, mocks).

**Traceability:** Implements [spec 001](../specs/001-messaging-core.md) §3–§6, §9 (memory); see ADR [0001](../adrs/0001-message-payload-typing.md), [0002](../adrs/0002-adapter-spi.md), [0004](../adrs/0004-clockwork-dependency.md).

## Global Constraints

- **Go 1.25 exact.** `go.mod` has `go 1.25`; build/test with `GOTOOLCHAIN=go1.25.0`. No features newer than 1.25.
- **Core = stdlib + `clockwork` only.** No other third-party import in non-test code. Test-only deps (`testify`, `goleak`, `mock`) are fine.
- **Blackbox tests only.** Every `_test.go` is `package msgin_test` (or `<pkg>_test`) and exercises the exported API. Export any sentinel a test must `errors.Is`.
- **Table tests use the assert-closure form** (project `table-test` skill): each case carries `assert func(t *testing.T, …)`, never `want`/`wantErr` fields. Use `t.Context()`, not `context.Background()`.
- **Mocks via `go.uber.org/mock`** (`mockgen --typed`), placed beside the interface (project `use-mockgen` skill).
- **Every consumer test asserts no goroutine leaks** with `goleak`.
- **Injectable time via `clockwork.Clock`** used directly; tests use `clockwork.NewFakeClock()`.
- **Module path:** `github.com/kartaladev/msgin`. Package `msgin` at repo root; memory adapter at `adapter/memory` (package `memory`).

---

## File Structure

- `message.go` — `Message[T]`, `Headers`, `New`, options, reserved header keys.
- `errors.go` — exported sentinels.
- `codec.go` — `PayloadCodec[T]`, `JSONPayloadCodec[T]`.
- `spi.go` — `Delivery`, `PollingSource`, `StreamingSource`, `OutboundAdapter`, `NativeReliability`, `LiveValueSource`.
- `producer.go` — `Producer[T]`, `NewProducer`, producer options.
- `consumer.go` — `Consumer[T]`, `Handler[T]`, `NewConsumer`, consumer options, the minimal run loop.
- `adapter/memory/memory.go` — the in-memory adapter (`Source`, `OutboundAdapter`, `LiveValueSource`).
- Test files alongside each (`*_test.go`, blackbox).

---

### Task 1: Headers (immutable) and reserved keys

**Files:**
- Create: `message.go` (Headers portion)
- Test: `message_test.go`

**Interfaces:**
- Produces: `type Headers` (opaque, immutable); `func (Headers) Get(key string) (any, bool)`; `func (Headers) String(key string) (string, bool)`; `func (Headers) Int(key string) (int, bool)`; `func (Headers) Time(key string) (time.Time, bool)`; `func (Headers) All() iter.Seq2[string, any]`; reserved-key consts `HeaderID = "msgin.id"`, `HeaderTimestamp = "msgin.timestamp"`, `HeaderContentType = "msgin.content-type"`, `HeaderCorrelationID = "msgin.correlation-id"`, `HeaderDeliveryCount = "msgin.delivery-count"`.

- [ ] **Step 1: Write the failing test**

```go
// message_test.go
package msgin_test

import (
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaders_TypedAccessors(t *testing.T) {
	h := msgin.NewHeaders(map[string]any{
		"s":                    "hello",
		"n":                    42,
		msgin.HeaderTimestamp:  time.Unix(1000, 0),
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

func TestHeaders_Immutable_AllDoesNotExposeBackingMap(t *testing.T) {
	src := map[string]any{"k": "v"}
	h := msgin.NewHeaders(src)
	// Mutating the source map must not affect the Headers snapshot.
	src["k"] = "mutated"
	v, ok := h.String("k")
	require.True(t, ok)
	assert.Equal(t, "v", v)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestHeaders -v`
Expected: FAIL — `undefined: msgin.NewHeaders` / `msgin.Headers`.

- [ ] **Step 3: Write minimal implementation**

```go
// message.go
package msgin

import (
	"iter"
	"maps"
	"time"
)

// Reserved header keys live under the "msgin." namespace.
const (
	HeaderID            = "msgin.id"
	HeaderTimestamp     = "msgin.timestamp"
	HeaderContentType   = "msgin.content-type"
	HeaderCorrelationID = "msgin.correlation-id"
	HeaderDeliveryCount = "msgin.delivery-count"
)

// Headers is an immutable set of message metadata. The backing map is never
// handed out mutably; construction copies the input.
type Headers struct {
	m map[string]any
}

// NewHeaders returns immutable Headers copied from m (nil m yields empty Headers).
func NewHeaders(m map[string]any) Headers {
	return Headers{m: maps.Clone(m)}
}

func (h Headers) Get(key string) (any, bool) {
	v, ok := h.m[key]
	return v, ok
}

func (h Headers) String(key string) (string, bool) {
	v, ok := h.m[key].(string)
	return v, ok
}

func (h Headers) Int(key string) (int, bool) {
	v, ok := h.m[key].(int)
	return v, ok
}

func (h Headers) Time(key string) (time.Time, bool) {
	v, ok := h.m[key].(time.Time)
	return v, ok
}

// All iterates the headers read-only (no mutable map is exposed).
func (h Headers) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range h.m {
			if !yield(k, v) {
				return
			}
		}
	}
}

// with returns a copy with key=v set (copy-on-write; used by Message.WithHeader).
func (h Headers) with(key string, v any) Headers {
	nm := maps.Clone(h.m)
	if nm == nil {
		nm = make(map[string]any, 1)
	}
	nm[key] = v
	return Headers{m: nm}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestHeaders -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add message.go message_test.go
git commit -m "feat: immutable Headers with typed accessors" \
  -m "Spec: 001" -m "ADR: 0001"
```

---

### Task 2: Exported error sentinels

**Files:**
- Create: `errors.go`
- Test: `errors_test.go`

**Interfaces:**
- Produces: `var ErrPayloadType`, `ErrPayloadDecode`, `ErrNilAdapter`, `ErrNoPayloadCodec`, `ErrUnexpectedCodec`, `ErrInvalidConcurrency`, `ErrUnsupportedSource` (all `error`).

- [ ] **Step 1: Write the failing test**

```go
// errors_test.go
package msgin_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
)

func TestSentinels_WrapAndCompare(t *testing.T) {
	sentinels := []error{
		msgin.ErrPayloadType, msgin.ErrPayloadDecode, msgin.ErrNilAdapter,
		msgin.ErrNoPayloadCodec, msgin.ErrUnexpectedCodec,
		msgin.ErrInvalidConcurrency, msgin.ErrUnsupportedSource,
	}
	for _, s := range sentinels {
		t.Run(s.Error(), func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", s)
			assert.True(t, errors.Is(wrapped, s))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestSentinels -v`
Expected: FAIL — `undefined: msgin.ErrPayloadType` (etc.).

- [ ] **Step 3: Write minimal implementation**

```go
// errors.go
package msgin

import "errors"

var (
	// ErrPayloadType is returned when a Message[any] payload cannot be asserted to T.
	ErrPayloadType = errors.New("msgin: payload is not of the expected type")
	// ErrPayloadDecode is returned when a wire payload ([]byte) cannot be decoded into T.
	ErrPayloadDecode = errors.New("msgin: payload decode failed")
	// ErrNilAdapter is returned by constructors when a required adapter is nil.
	ErrNilAdapter = errors.New("msgin: adapter is nil")
	// ErrNoPayloadCodec is returned when a wire source is used without a PayloadCodec.
	ErrNoPayloadCodec = errors.New("msgin: wire source requires a payload codec")
	// ErrUnexpectedCodec is returned when a live-value source (memory) is given a codec.
	ErrUnexpectedCodec = errors.New("msgin: live-value source must not have a payload codec")
	// ErrInvalidConcurrency is returned when WithConcurrency is < 1.
	ErrInvalidConcurrency = errors.New("msgin: concurrency must be >= 1")
	// ErrUnsupportedSource is returned when a Source is neither Polling nor Streaming.
	ErrUnsupportedSource = errors.New("msgin: source implements neither PollingSource nor StreamingSource")
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestSentinels -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add errors.go errors_test.go
git commit -m "feat: exported error sentinels" -m "Spec: 001" -m "ADR: 0002"
```

---

### Task 3: Message[T], New, and options

**Files:**
- Modify: `message.go` (append Message + New + options)
- Test: `message_test.go` (append)

**Interfaces:**
- Consumes: `Headers` (Task 1), `HeaderID`/`HeaderTimestamp` consts.
- Produces: `type Message[T any]`; `func New[T any](payload T, opts ...MessageOption) Message[T]`; methods `Payload() T`, `ID() string`, `Header(key string) (any, bool)`, `Headers() Headers`, `WithHeader(key string, v any) Message[T]`; `type MessageOption`; `func WithClock(c clockwork.Clock) MessageOption`; `func WithID(id string) MessageOption`; `func WithHeaders(m map[string]any) MessageOption`.

- [ ] **Step 1: Write the failing test**

```go
// message_test.go (append)
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
```

Add imports `"github.com/jonboulle/clockwork"` to the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestNew_|TestMessage_' -v`
Expected: FAIL — `undefined: msgin.New` / `msgin.MessageOption`.

- [ ] **Step 3: Write minimal implementation**

```go
// message.go (append)
import (
	"crypto/rand"
	"encoding/hex"

	"github.com/jonboulle/clockwork"
)

// Message is the immutable EIP envelope: a typed payload plus header metadata.
type Message[T any] struct {
	payload T
	headers Headers
}

type msgConfig struct {
	clock   clockwork.Clock
	id      string
	headers map[string]any
}

// MessageOption configures New.
type MessageOption func(*msgConfig)

// WithClock injects the clock used to stamp msgin.timestamp (default: real clock).
func WithClock(c clockwork.Clock) MessageOption { return func(o *msgConfig) { o.clock = c } }

// WithID sets an explicit msgin.id (default: a random 128-bit hex id).
func WithID(id string) MessageOption { return func(o *msgConfig) { o.id = id } }

// WithHeaders seeds additional headers.
func WithHeaders(m map[string]any) MessageOption { return func(o *msgConfig) { o.headers = m } }

// New builds an immutable Message, always stamping msgin.id and msgin.timestamp.
func New[T any](payload T, opts ...MessageOption) Message[T] {
	cfg := msgConfig{clock: clockwork.NewRealClock()}
	for _, opt := range opts {
		opt(&cfg)
	}
	m := map[string]any{}
	for k, v := range cfg.headers {
		m[k] = v
	}
	if cfg.id == "" {
		cfg.id = randomID()
	}
	m[HeaderID] = cfg.id
	m[HeaderTimestamp] = cfg.clock.Now()
	return Message[T]{payload: payload, headers: Headers{m: m}}
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (m Message[T]) Payload() T                 { return m.payload }
func (m Message[T]) Headers() Headers           { return m.headers }
func (m Message[T]) Header(k string) (any, bool) { return m.headers.Get(k) }

func (m Message[T]) ID() string {
	id, _ := m.headers.String(HeaderID)
	return id
}

// WithHeader returns a copy of the message with key=v set (copy-on-write).
func (m Message[T]) WithHeader(key string, v any) Message[T] {
	return Message[T]{payload: m.payload, headers: m.headers.with(key, v)}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestNew_|TestMessage_' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add message.go message_test.go
git commit -m "feat: immutable Message[T] with clock-stamped id/timestamp" \
  -m "Spec: 001" -m "ADR: 0001, 0004"
```

---

### Task 4: PayloadCodec and JSON default

**Files:**
- Create: `codec.go`
- Test: `codec_test.go`

**Interfaces:**
- Produces: `type PayloadCodec[T any] interface { Encode(T) ([]byte, error); Decode([]byte) (T, error) }`; `type JSONPayloadCodec[T any] struct{}` implementing it.

- [ ] **Step 1: Write the failing test**

```go
// codec_test.go
package msgin_test

import (
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type order struct {
	ID    string `json:"id"`
	Total int    `json:"total"`
}

func TestJSONPayloadCodec_RoundTrip(t *testing.T) {
	c := msgin.JSONPayloadCodec[order]{}

	tests := []struct {
		name   string
		in     order
		assert func(t *testing.T, out order, b []byte, err error)
	}{
		{"round trips", order{ID: "o1", Total: 5},
			func(t *testing.T, out order, b []byte, err error) {
				require.NoError(t, err)
				assert.Equal(t, order{ID: "o1", Total: 5}, out)
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := c.Encode(tc.in)
			require.NoError(t, err)
			out, err := c.Decode(b)
			tc.assert(t, out, b, err)
		})
	}
}

func TestJSONPayloadCodec_DecodeError(t *testing.T) {
	c := msgin.JSONPayloadCodec[order]{}
	_, err := c.Decode([]byte("{not json"))
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestJSONPayloadCodec -v`
Expected: FAIL — `undefined: msgin.JSONPayloadCodec`.

- [ ] **Step 3: Write minimal implementation**

```go
// codec.go
package msgin

import "encoding/json"

// PayloadCodec (de)serializes a typed payload to/from bytes. It lives in the
// typed runtime (which knows T); wire adapters never see T (spec §3, ADR 0001).
type PayloadCodec[T any] interface {
	Encode(T) ([]byte, error)
	Decode([]byte) (T, error)
}

// JSONPayloadCodec is the default PayloadCodec using encoding/json.
type JSONPayloadCodec[T any] struct{}

func (JSONPayloadCodec[T]) Encode(v T) ([]byte, error) { return json.Marshal(v) }

func (JSONPayloadCodec[T]) Decode(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestJSONPayloadCodec -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add codec.go codec_test.go
git commit -m "feat: PayloadCodec and JSON default" -m "Spec: 001" -m "ADR: 0001"
```

---

### Task 5: The adapter SPI

**Files:**
- Create: `spi.go`
- Test: `spi_test.go` (compile-time interface-satisfaction assertions)

**Interfaces:**
- Consumes: `Message` (Task 3).
- Produces: `type Delivery`; `type PollingSource interface { Poll(ctx, max int) ([]Delivery, error) }`; `type StreamingSource interface { Stream(ctx, out chan<- Delivery) error }`; `type OutboundAdapter interface { Send(ctx, Message[any]) error }`; `type NativeReliability interface { NativeRedelivery() bool; NativeDeadLetter() bool }`; `type LiveValueSource interface { EmitsLiveValue() bool }`.

- [ ] **Step 1: Write the failing test**

```go
// spi_test.go
package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
)

// Compile-time proof that a minimal type can satisfy the SPI interfaces.
type stubSource struct{}

func (stubSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error { return nil }
func (stubSource) EmitsLiveValue() bool                                        { return true }

type stubOut struct{}

func (stubOut) Send(ctx context.Context, m msgin.Message[any]) error { return nil }

func TestSPI_InterfacesSatisfiable(t *testing.T) {
	var _ msgin.StreamingSource = stubSource{}
	var _ msgin.LiveValueSource = stubSource{}
	var _ msgin.OutboundAdapter = stubOut{}

	// Delivery is a struct with settle closures.
	d := msgin.Delivery{
		Msg:  msgin.New[any]("x"),
		Ack:  func(context.Context) error { return nil },
		Nack: func(context.Context, bool, time.Duration) error { return nil },
	}
	if err := d.Ack(t.Context()); err != nil {
		t.Fatalf("ack: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestSPI -v`
Expected: FAIL — `undefined: msgin.Delivery` / `msgin.StreamingSource`.

- [ ] **Step 3: Write minimal implementation**

```go
// spi.go
package msgin

import (
	"context"
	"time"
)

// Delivery is one received message plus the means to settle it. Msg.Payload is
// []byte for wire adapters or a live value for the in-memory adapter.
type Delivery struct {
	Msg  Message[any]
	Ack  func(ctx context.Context) error
	Nack func(ctx context.Context, requeue bool, delay time.Duration) error
}

// PollingSource is a pulled inbound adapter, driven by the runtime's Poller.
type PollingSource interface {
	Poll(ctx context.Context, max int) ([]Delivery, error)
}

// StreamingSource is a pushed inbound adapter that owns a blocking, cancellable loop.
type StreamingSource interface {
	Stream(ctx context.Context, out chan<- Delivery) error
}

// OutboundAdapter writes a message to the external system.
type OutboundAdapter interface {
	Send(ctx context.Context, msg Message[any]) error
}

// NativeReliability is an optional capability: two independent booleans (ADR 0002).
type NativeReliability interface {
	NativeRedelivery() bool
	NativeDeadLetter() bool
}

// LiveValueSource is an optional capability: a source emitting live Go values
// (in-memory) rather than []byte, so NewConsumer can enforce codec pairing.
type LiveValueSource interface {
	EmitsLiveValue() bool
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestSPI -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add spi.go spi_test.go
git commit -m "feat: adapter SPI (Delivery, sources, outbound, capabilities)" \
  -m "Spec: 001" -m "ADR: 0002"
```

---

### Task 6: In-memory adapter

**Files:**
- Create: `adapter/memory/memory.go`
- Create: `adapter/memory/doc.go` (package doc)
- Test: `adapter/memory/memory_test.go`

**Interfaces:**
- Consumes: `msgin.Delivery`, `msgin.StreamingSource`, `msgin.OutboundAdapter`, `msgin.LiveValueSource`, `msgin.Message`.
- Produces: `func New(opts ...Option) *Broker`; `func (*Broker) Stream(ctx, out chan<- msgin.Delivery) error`; `func (*Broker) EmitsLiveValue() bool`; `func (*Broker) Send(ctx, msgin.Message[any]) error`; `func WithBuffer(n int) Option`.

- [ ] **Step 1: Write the failing test**

```go
// adapter/memory/memory_test.go
package memory_test

import (
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestBroker_SendThenStreamDelivers(t *testing.T) {
	b := memory.New(memory.WithBuffer(4))

	require.NoError(t, b.Send(t.Context(), msgin.New[any]("hello", msgin.WithID("m1"))))

	out := make(chan msgin.Delivery, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = b.Stream(ctx, out) }()

	d := <-out
	assert.Equal(t, "hello", d.Msg.Payload())
	assert.Equal(t, "m1", d.Msg.ID())
	require.NoError(t, d.Ack(t.Context()))
}

func TestBroker_EmitsLiveValue(t *testing.T) {
	var _ msgin.LiveValueSource = memory.New()
	assert.True(t, memory.New().EmitsLiveValue())
}
```

Add import `"context"` to the test.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./adapter/memory/... -v`
Expected: FAIL — `undefined: memory.New`.

- [ ] **Step 3: Write minimal implementation**

```go
// adapter/memory/memory.go
package memory

import (
	"context"

	"github.com/kartaladev/msgin"
)

// Broker is an in-process point-to-point transport backed by a Go channel. It
// carries live Go values (no codec, zero-copy) and is the reference adapter and
// test double. Delivery guarantee: at-most-once.
type Broker struct {
	ch chan msgin.Message[any]
}

// Option configures a Broker.
type Option func(*Broker)

// WithBuffer sets the channel buffer size (default 0 — synchronous handoff).
func WithBuffer(n int) Option { return func(b *Broker) { b.ch = make(chan msgin.Message[any], n) } }

// New builds an in-memory Broker.
func New(opts ...Option) *Broker {
	b := &Broker{ch: make(chan msgin.Message[any])}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Send enqueues a message (outbound adapter).
func (b *Broker) Send(ctx context.Context, m msgin.Message[any]) error {
	select {
	case b.ch <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stream delivers messages until ctx is cancelled (streaming source). Ack/Nack
// are no-ops for at-most-once; Nack with requeue re-enqueues.
func (b *Broker) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-b.ch:
			d := msgin.Delivery{
				Msg:  m,
				Ack:  func(context.Context) error { return nil },
				Nack: b.nackFunc(m),
			}
			select {
			case out <- d:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (b *Broker) nackFunc(m msgin.Message[any]) func(context.Context, bool, time.Duration) error {
	return func(ctx context.Context, requeue bool, _ time.Duration) error {
		if !requeue {
			return nil
		}
		return b.Send(ctx, m)
	}
}

// EmitsLiveValue reports that this source carries live Go values (no codec).
func (b *Broker) EmitsLiveValue() bool { return true }
```

Add `"time"` to the imports.

```go
// adapter/memory/doc.go
// Package memory is an in-process, at-most-once msgin adapter backed by a Go
// channel. It carries live Go values (no payload codec) and is the reference
// adapter and the test double for the core.
package memory
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./adapter/memory/... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add adapter/memory/
git commit -m "feat(memory): in-memory at-most-once adapter" -m "Spec: 001" -m "ADR: 0002"
```

---

### Task 7: Producer[T] with codec pairing

**Files:**
- Create: `producer.go`
- Test: `producer_test.go`

**Interfaces:**
- Consumes: `OutboundAdapter`, `PayloadCodec[T]`, `LiveValueSource`, `Message`, sentinels.
- Produces: `type Producer[T any] interface { Send(ctx, Message[T]) error }`; `func NewProducer[T any](out OutboundAdapter, opts ...ProducerOption[T]) (Producer[T], error)`; `type ProducerOption[T any]`; `func WithProducerCodec[T any](c PayloadCodec[T]) ProducerOption[T]`.

**Design note:** The producer lifts `Message[T]`→`Message[any]`. If the outbound adapter is a `LiveValueSource` it passes the live value (no codec); otherwise it encodes `T`→`[]byte` via the codec (default JSON). Mismatch (live-value + explicit codec, or wire + no codec) is a construction error.

- [ ] **Step 1: Write the failing test**

```go
// producer_test.go
package msgin_test

import (
	"context"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wireOut is a non-live-value (wire) outbound adapter capturing the last payload.
type wireOut struct{ last msgin.Message[any] }

func (w *wireOut) Send(_ context.Context, m msgin.Message[any]) error { w.last = m; return nil }

func TestNewProducer_CodecPairing(t *testing.T) {
	tests := []struct {
		name   string
		out    msgin.OutboundAdapter
		opts   []msgin.ProducerOption[order]
		assert func(t *testing.T, p msgin.Producer[order], err error)
	}{
		{"live-value adapter needs no codec", memory.New(), nil,
			func(t *testing.T, p msgin.Producer[order], err error) {
				require.NoError(t, err)
			}},
		{"live-value adapter with codec is rejected", memory.New(),
			[]msgin.ProducerOption[order]{msgin.WithProducerCodec[order](msgin.JSONPayloadCodec[order]{})},
			func(t *testing.T, p msgin.Producer[order], err error) {
				assert.ErrorIs(t, err, msgin.ErrUnexpectedCodec)
			}},
		{"nil adapter rejected", nil, nil,
			func(t *testing.T, p msgin.Producer[order], err error) {
				assert.ErrorIs(t, err, msgin.ErrNilAdapter)
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := msgin.NewProducer[order](tc.out, tc.opts...)
			tc.assert(t, p, err)
		})
	}
}

func TestProducer_WireEncodesToBytes(t *testing.T) {
	w := &wireOut{}
	p, err := msgin.NewProducer[order](w) // wire adapter, default JSON codec
	require.NoError(t, err)

	require.NoError(t, p.Send(t.Context(), msgin.New(order{ID: "o1", Total: 3})))
	b, ok := w.last.Payload().([]byte)
	require.True(t, ok, "wire payload must be []byte")
	assert.JSONEq(t, `{"id":"o1","total":3}`, string(b))
}

func TestProducer_LiveValuePassesThrough(t *testing.T) {
	// memory is a LiveValueSource: the live order value passes through unencoded.
	b := memory.New(memory.WithBuffer(1))
	p, err := msgin.NewProducer[order](b)
	require.NoError(t, err)
	require.NoError(t, p.Send(t.Context(), msgin.New(order{ID: "o2"})))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestNewProducer|TestProducer_' -v`
Expected: FAIL — `undefined: msgin.NewProducer`.

- [ ] **Step 3: Write minimal implementation**

```go
// producer.go
package msgin

import "context"

// Producer sends typed messages into a flow.
type Producer[T any] interface {
	Send(ctx context.Context, msg Message[T]) error
}

// ProducerOption configures NewProducer.
type ProducerOption[T any] func(*producerConfig[T])

type producerConfig[T any] struct {
	codec    PayloadCodec[T]
	codecSet bool
}

// WithProducerCodec sets the payload codec for a wire adapter (default JSON).
func WithProducerCodec[T any](c PayloadCodec[T]) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.codec = c; o.codecSet = true }
}

type producer[T any] struct {
	out       OutboundAdapter
	codec     PayloadCodec[T]
	liveValue bool
}

// NewProducer builds a Producer, validating codec pairing at construction.
func NewProducer[T any](out OutboundAdapter, opts ...ProducerOption[T]) (Producer[T], error) {
	if out == nil {
		return nil, ErrNilAdapter
	}
	var cfg producerConfig[T]
	for _, opt := range opts {
		opt(&cfg)
	}
	live := isLiveValue(out)
	if live && cfg.codecSet {
		return nil, ErrUnexpectedCodec
	}
	codec := cfg.codec
	if !live && codec == nil {
		codec = JSONPayloadCodec[T]{}
	}
	return &producer[T]{out: out, codec: codec, liveValue: live}, nil
}

func (p *producer[T]) Send(ctx context.Context, msg Message[T]) error {
	if p.liveValue {
		return p.out.Send(ctx, Message[any]{payload: any(msg.payload), headers: msg.headers})
	}
	b, err := p.codec.Encode(msg.payload)
	if err != nil {
		return err
	}
	return p.out.Send(ctx, Message[any]{payload: any(b), headers: msg.headers})
}

// isLiveValue reports whether an adapter emits/consumes live Go values.
func isLiveValue(a any) bool {
	lv, ok := a.(LiveValueSource)
	return ok && lv.EmitsLiveValue()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestNewProducer|TestProducer_' -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add producer.go producer_test.go
git commit -m "feat: Producer[T] with construction-time codec pairing" \
  -m "Spec: 001" -m "ADR: 0001, 0002"
```

---

### Task 8: Consumer[T] — minimal run loop (streaming ingest + worker pool + Ack/Nack)

**Files:**
- Create: `consumer.go`
- Test: `consumer_test.go`

**Interfaces:**
- Consumes: `StreamingSource`, `PollingSource` (type-switch), `PayloadCodec[T]`, `LiveValueSource`, `Delivery`, `Handler[T]`, sentinels, memory adapter (test).
- Produces: `type Handler[T any] func(ctx, Message[T]) error`; `type Consumer[T any] interface { Run(ctx) error }`; `func NewConsumer[T any](src any, h Handler[T], opts ...ConsumerOption[T]) (Consumer[T], error)`; `type ConsumerOption[T any]`; `func WithConcurrency[T any](n int) ConsumerOption[T]`; `func WithConsumerCodec[T any](c PayloadCodec[T]) ConsumerOption[T]`.

**Design note (scope):** This is the *minimal* runtime. It type-switches the source; for a `StreamingSource` it runs `Stream` in an owned goroutine feeding a bounded channel, and a worker pool of N goroutines decodes → runs the handler → `Ack` on success, `Nack(requeue=true, 0)` on error. **Retry/dead-letter/invalid-message (Plan 002) and credit/flow-control (Plan 003) are NOT in scope here.** `PollingSource` returns `ErrUnsupportedSource` for now (wired in Plan 004). Decode failure → `Nack` (Plan 002 will route it to the invalid channel).

- [ ] **Step 1: Write the failing test**

```go
// consumer_test.go
package msgin_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestConsumer_StreamingDeliversToHandler(t *testing.T) {
	b := memory.New(memory.WithBuffer(8))
	p, err := msgin.NewProducer[order](b)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		require.NoError(t, p.Send(t.Context(), msgin.New(order{ID: "o"})))
	}

	var (
		mu   sync.Mutex
		seen int
	)
	h := func(_ context.Context, m msgin.Message[order]) error {
		mu.Lock()
		seen++
		mu.Unlock()
		return nil
	}

	c, err := msgin.NewConsumer[order](b, h) // memory ⇒ no codec (live value)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen == 3
	}, time.Second, 5*time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)
}

func TestNewConsumer_Validation(t *testing.T) {
	tests := []struct {
		name   string
		src    any
		opts   []msgin.ConsumerOption[order]
		assert func(t *testing.T, err error)
	}{
		{"nil source", nil, nil,
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNilAdapter) }},
		{"concurrency < 1", memory.New(),
			[]msgin.ConsumerOption[order]{msgin.WithConcurrency[order](0)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidConcurrency) }},
		{"live-value source with codec", memory.New(),
			[]msgin.ConsumerOption[order]{msgin.WithConsumerCodec[order](msgin.JSONPayloadCodec[order]{})},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrUnexpectedCodec) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := func(context.Context, msgin.Message[order]) error { return nil }
			_, err := msgin.NewConsumer[order](tc.src, h, tc.opts...)
			tc.assert(t, err)
		})
	}
}
```

(Remove the duplicate `TestMain` if `producer_test.go`/others already declare one — keep exactly one `TestMain` per `_test` package; place it in a single file, e.g. `consumer_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_|TestNewConsumer' -v`
Expected: FAIL — `undefined: msgin.NewConsumer`.

- [ ] **Step 3: Write minimal implementation**

```go
// consumer.go
package msgin

import (
	"context"
	"sync"
)

// Handler consumes a typed message. nil = success (Ack); non-nil = failure.
type Handler[T any] func(ctx context.Context, msg Message[T]) error

// Consumer runs a flow until its context is cancelled.
type Consumer[T any] interface {
	Run(ctx context.Context) error
}

// ConsumerOption configures NewConsumer.
type ConsumerOption[T any] func(*consumerConfig[T])

type consumerConfig[T any] struct {
	concurrency int
	codec       PayloadCodec[T]
	codecSet    bool
	buffer      int
}

// WithConcurrency sets the worker-pool size (default 1).
func WithConcurrency[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.concurrency = n }
}

// WithConsumerCodec sets the payload codec for a wire source (default JSON).
func WithConsumerCodec[T any](c PayloadCodec[T]) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.codec = c; o.codecSet = true }
}

type consumer[T any] struct {
	src       StreamingSource
	handler   Handler[T]
	codec     PayloadCodec[T]
	liveValue bool
	workers   int
	buffer    int
}

// NewConsumer validates the source and options, and builds a Consumer.
func NewConsumer[T any](src any, h Handler[T], opts ...ConsumerOption[T]) (Consumer[T], error) {
	if src == nil {
		return nil, ErrNilAdapter
	}
	cfg := consumerConfig[T]{concurrency: 1, buffer: 1}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.concurrency < 1 {
		return nil, ErrInvalidConcurrency
	}
	live := isLiveValue(src)
	if live && cfg.codecSet {
		return nil, ErrUnexpectedCodec
	}
	codec := cfg.codec
	if !live && codec == nil {
		codec = JSONPayloadCodec[T]{}
	}

	stream, ok := src.(StreamingSource)
	if !ok {
		// PollingSource is wired in Plan 004; anything else is unsupported.
		if _, isPoll := src.(PollingSource); isPoll {
			return nil, ErrUnsupportedSource // TODO(Plan 004): drive via the Poller
		}
		return nil, ErrUnsupportedSource
	}
	return &consumer[T]{
		src: stream, handler: h, codec: codec, liveValue: live,
		workers: cfg.concurrency, buffer: cfg.buffer,
	}, nil
}

func (c *consumer[T]) Run(ctx context.Context) error {
	deliveries := make(chan Delivery, c.buffer)

	var wg sync.WaitGroup
	wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for d := range deliveries {
				c.dispatch(ctx, d)
			}
		}()
	}

	err := c.src.Stream(ctx, deliveries) // blocks until ctx is cancelled
	close(deliveries)
	wg.Wait()
	return err
}

// dispatch decodes, runs the handler, and settles (minimal: Ack on success,
// Nack+requeue on error). Retry/DLQ/invalid come in Plan 002.
func (c *consumer[T]) dispatch(ctx context.Context, d Delivery) {
	payload, err := c.decode(d.Msg)
	if err != nil {
		_ = d.Nack(ctx, true, 0) // Plan 002: route to invalid-message channel
		return
	}
	msg := Message[T]{payload: payload, headers: d.Msg.headers}
	if herr := c.safeHandle(ctx, msg); herr != nil {
		_ = d.Nack(ctx, true, 0) // Plan 002: retry/backoff/DLQ
		return
	}
	_ = d.Ack(ctx)
}

func (c *consumer[T]) decode(m Message[any]) (T, error) {
	if c.liveValue {
		v, ok := m.payload.(T)
		if !ok {
			var zero T
			return zero, ErrPayloadType
		}
		return v, nil
	}
	b, ok := m.payload.([]byte)
	if !ok {
		var zero T
		return zero, ErrPayloadType
	}
	v, err := c.codec.Decode(b)
	if err != nil {
		var zero T
		return zero, ErrPayloadDecode
	}
	return v, nil
}

// safeHandle recovers a panicking handler and reports it as an error.
func (c *consumer[T]) safeHandle(ctx context.Context, msg Message[T]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrPayloadType // placeholder; Plan 002 defines a panic error + hook
		}
	}()
	return c.handler(ctx, msg)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add consumer.go consumer_test.go
git commit -m "feat: minimal Consumer[T] runtime (streaming ingest + worker pool)" \
  -m "Spec: 001" -m "ADR: 0001, 0002"
```

---

### Task 9: Wire up module test deps and CI-green whole-package run

**Files:**
- Modify: `go.mod` (add test-only requires), create `go.sum` via tidy.

- [ ] **Step 1: Add test dependencies**

Run:
```bash
GOTOOLCHAIN=go1.25.0 go get github.com/jonboulle/clockwork@latest
GOTOOLCHAIN=go1.25.0 go get github.com/stretchr/testify@latest
GOTOOLCHAIN=go1.25.0 go get go.uber.org/goleak@latest
GOTOOLCHAIN=go1.25.0 go mod tidy
```

- [ ] **Step 2: Run the whole suite race-clean**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race`
Expected: PASS (all packages), no goroutine leaks.

- [ ] **Step 3: Vet + format**

Run: `GOTOOLCHAIN=go1.25.0 go vet ./... && gofmt -l .`
Expected: no output from either.

- [ ] **Step 4: Verify only clockwork is a non-test core dependency**

Run: `GOTOOLCHAIN=go1.25.0 go mod why github.com/jonboulle/clockwork`
Expected: reachable from non-test code (used by `message.go`). `testify`/`goleak` should appear only under test.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "build: pin test dependencies (clockwork, testify, goleak)" -m "Spec: 001"
```

---

## Self-Review

**Spec coverage (§3–§6, §9 memory):**
- §3 two encodings → Tasks 4 (codec), 7 (producer lifts T→any/[]byte), 8 (consumer decode). ✓
- §4 Message/Headers immutability → Tasks 1, 3. ✓
- §5 caller API + constructors return errors → Tasks 7, 8. ✓ (full option set — retry/observability — deferred to Plan 002/003, noted in scope.)
- §6 SPI + no sealed marker + LiveValueSource discriminator + codec pairing → Tasks 5, 7, 8. ✓
- §9 memory (live value, zero-copy, at-most-once) → Task 6. ✓
- **Deferred (documented):** settlement switch/retry/DLQ/invalid (Plan 002), credit/flow-control (Plan 003), Poller + PollingSource driving (Plan 004). `Nack` here = requeue only.

**Placeholder scan:** two `TODO(Plan …)` markers are intentional forward-references to later plans, not missing content in this plan's scope. The panic-error placeholder in `safeHandle` is explicitly deferred to Plan 002 (which defines the panic error + hook).

**Type consistency:** `Message[any]{payload:…, headers:…}` uses unexported fields — the producer/consumer are in package `msgin`, so this is legal (same package). `isLiveValue`/`LiveValueSource.EmitsLiveValue` names consistent across Tasks 5–8. `Delivery.Nack` signature `(ctx, bool, time.Duration)` consistent across Tasks 5, 6, 8.

---

## Notes for Plan 002 (next)

Plan 002 replaces `dispatch`'s minimal settle with the guarded switch: `RetryPolicy`, closed-form backoff, attempt counting (`msgin.delivery-count`), the invalid-message channel (decode failures + permanent errors), dead-letter, the panic error, observability (`*slog.Logger` + `Hooks`), and shutdown with `WithShutdownTimeout`. It also adds `WithRetryPolicy`, `WithInvalidMessageSink`, `WithLogger`, `WithHooks` consumer options.
