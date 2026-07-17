package msgin

import (
	"crypto/rand"
	"encoding/hex"
	"iter"
	"maps"
	"time"

	"github.com/jonboulle/clockwork"
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
// handed out mutably; construction copies the input. Immutability is
// shallow: the map itself is copied, but header values are not deep-cloned,
// so callers should store immutable value types.
type Headers struct {
	m map[string]any
}

// NewHeaders returns immutable Headers copied from m (nil m yields empty
// Headers). The copy is shallow: header values are not deep-cloned, so
// callers should store immutable value types.
func NewHeaders(m map[string]any) Headers {
	return Headers{m: maps.Clone(m)}
}

// Get returns the raw value for key and whether it was present.
func (h Headers) Get(key string) (any, bool) {
	v, ok := h.m[key]
	return v, ok
}

// String returns the value for key as a string, and whether it was present
// and held a string.
func (h Headers) String(key string) (string, bool) {
	v, ok := h.m[key].(string)
	return v, ok
}

// Int returns the value for key as an int, and whether it was present and
// held an int.
func (h Headers) Int(key string) (int, bool) {
	v, ok := h.m[key].(int)
	return v, ok
}

// Time returns the value for key as a time.Time, and whether it was present
// and held a time.Time.
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

// Message is the immutable EIP envelope: a typed payload plus header
// metadata. Every Message is stamped with a msgin.id and msgin.timestamp by
// New; transformers and enrichers return a new Message rather than mutating
// one in place, so a Message shared across a pub-sub channel is safe to read
// concurrently.
type Message[T any] struct {
	payload T
	headers Headers
}

// msgConfig accumulates MessageOption settings before New builds a Message.
type msgConfig struct {
	clock   clockwork.Clock
	id      string
	headers map[string]any
}

// MessageOption configures a Message constructed by New.
type MessageOption func(*msgConfig)

// WithClock injects the clock New uses to stamp msgin.timestamp. The default
// is the real wall clock; tests typically inject a clockwork.FakeClock. A nil
// clock is a no-op (leaves the real-clock default in place) rather than a
// caller-triggered nil-panic on the next New call (no panic on caller input).
func WithClock(c clockwork.Clock) MessageOption {
	return func(o *msgConfig) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithID sets an explicit msgin.id instead of New's default random 128-bit
// hex id. An empty string is treated as unset: New falls back to generating
// a random id rather than stamping an empty msgin.id.
func WithID(id string) MessageOption {
	return func(o *msgConfig) { o.id = id }
}

// WithHeaders seeds additional headers on the Message. The reserved
// msgin.id and msgin.timestamp keys are always overwritten by New,
// regardless of what is passed here.
func WithHeaders(m map[string]any) MessageOption {
	return func(o *msgConfig) { o.headers = m }
}

// New builds an immutable Message wrapping payload, always stamping
// msgin.id (random unless WithID is given) and msgin.timestamp (from the
// clock, real by default).
//
// New is the PRODUCING-path constructor: it always stamps a fresh id and
// timestamp. An inbound adapter reconstructing a message that already exists
// in an external system (its id/timestamp were framed at publish time and
// decoded back from storage) must use NewMessage instead, which preserves the
// pre-built Headers verbatim.
func New[T any](payload T, opts ...MessageOption) Message[T] {
	cfg := msgConfig{clock: clockwork.NewRealClock()}
	for _, opt := range opts {
		opt(&cfg)
	}
	m := make(map[string]any, len(cfg.headers)+2)
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

// NewMessage wraps an explicit payload and a pre-built Headers set as a
// Message, WITHOUT stamping msgin.id/msgin.timestamp — for adapters
// reconstructing a message that already exists in an external system (its id,
// timestamp, and custom headers were framed at publish time and decoded back
// from storage). Contrast New, which STAMPS a fresh id + timestamp for a
// newly-produced message.
//
// The Headers are used verbatim: NewMessage neither adds nor overwrites any
// reserved key, so a caller building a FRESH message this way (rather than
// reconstructing a persisted one) is responsible for supplying msgin.id and
// msgin.timestamp in headers if it needs them — prefer New for that case.
func NewMessage[T any](payload T, headers Headers) Message[T] {
	return Message[T]{payload: payload, headers: headers}
}

// randomID returns a random 128-bit id, hex-encoded, used as the default
// msgin.id when New is not given an explicit WithID.
func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Payload returns the message's typed payload.
func (m Message[T]) Payload() T { return m.payload }

// Headers returns the message's immutable header set.
func (m Message[T]) Headers() Headers { return m.headers }

// Header returns the raw header value for key and whether it was present.
func (m Message[T]) Header(key string) (any, bool) { return m.headers.Get(key) }

// ID returns the message's msgin.id header.
func (m Message[T]) ID() string {
	id, _ := m.headers.String(HeaderID)
	return id
}

// WithHeader returns a copy of the message with key=v set on its headers,
// leaving the receiver unchanged (copy-on-write).
func (m Message[T]) WithHeader(key string, v any) Message[T] {
	return Message[T]{payload: m.payload, headers: m.headers.with(key, v)}
}
