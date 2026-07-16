package msgin

import (
	"context"
	"fmt"
)

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
	codec, live, err := resolveCodec[T](out, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
	}
	return &producer[T]{out: out, codec: codec, liveValue: live}, nil
}

// Send lifts msg from Message[T] to Message[any] and writes it to the
// outbound adapter: live-value adapters (e.g. memory) receive the payload
// unencoded, wire adapters receive it encoded to []byte via the codec.
func (p *producer[T]) Send(ctx context.Context, msg Message[T]) error {
	if p.liveValue {
		return p.out.Send(ctx, Message[any]{payload: msg.payload, headers: msg.headers})
	}
	b, err := p.codec.Encode(msg.payload)
	if err != nil {
		return fmt.Errorf("msgin: producer encode failed: %w", err)
	}
	return p.out.Send(ctx, Message[any]{payload: any(b), headers: msg.headers})
}

// isLiveValue reports whether an adapter emits/consumes live Go values.
func isLiveValue(a any) bool {
	lv, ok := a.(LiveValueSource)
	return ok && lv.EmitsLiveValue()
}

// resolveCodec applies the codec-pairing rules shared by NewProducer and
// NewConsumer: a live-value adapter must not be given an explicit codec
// (ErrUnexpectedCodec); a wire adapter with no codec defaults to JSON. It
// returns the resolved codec and whether adapter is a live-value adapter.
func resolveCodec[T any](adapter any, codec PayloadCodec[T], codecSet bool) (PayloadCodec[T], bool, error) {
	live := isLiveValue(adapter)
	if live && codecSet {
		return nil, live, ErrUnexpectedCodec
	}
	if !live && codec == nil {
		codec = JSONPayloadCodec[T]{}
	}
	return codec, live, nil
}
