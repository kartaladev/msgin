package msgin

import "fmt"

// PayloadOf asserts m's payload to T, returning a typed Message[T] with the same
// headers (no re-stamp). On mismatch it wraps the package sentinel ErrPayloadType
// with the wanted/actual types; because IsPermanent classifies ErrPayloadType as
// permanent, the driving Consumer diverts it to the invalid-message channel — or
// to the dead-letter sink when none is configured ([Permanent] states all three
// arms) — never a panic. PayloadOf[any] succeeds for any non-nil payload. An untyped-nil
// payload (a message built with New[any](nil)) has no dynamic type, so the
// assertion fails and PayloadOf returns ErrPayloadType — even for T = any.
func PayloadOf[T any](m Message[any]) (Message[T], error) {
	v, ok := m.Payload().(T)
	if !ok {
		return Message[T]{}, fmt.Errorf("%w: want %T, got %T", ErrPayloadType, *new(T), m.Payload())
	}
	return NewMessage[T](v, m.Headers()), nil
}

// WithPayload returns a new Message carrying payload but the SAME headers as m
// (id, timestamp, correlation-id, custom keys preserved). It is the
// header-propagating way to write a Transformer/Activator body — prefer it over
// New, which stamps a fresh id and drops the incoming headers.
func WithPayload[A, B any](m Message[A], payload B) Message[B] {
	return NewMessage[B](payload, m.Headers())
}
