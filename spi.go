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
