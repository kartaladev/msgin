package msgin

import (
	"context"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
)

// Producer sends typed messages into a flow.
type Producer[T any] interface {
	Send(ctx context.Context, msg Message[T]) error
	// SendAfter delivers msg so it becomes consumable only after delay elapses,
	// when the underlying adapter supports scheduled delivery (ScheduledSender);
	// otherwise it returns ErrScheduledSendUnsupported. A delay <= 0 delivers
	// immediately (identical to Send) — a negative delay is normalized to 0. The
	// delay is relative and skew-free: the adapter's store computes visibility as
	// now+delay.
	SendAfter(ctx context.Context, msg Message[T], delay time.Duration) error
	// SendAt delivers msg so it becomes consumable no earlier than the wall-clock
	// time t, when the adapter supports scheduled delivery; otherwise it returns
	// ErrScheduledSendUnsupported. It is sugar over SendAfter, computing
	// delay = t - now with the producer's clock; a t already in the past delivers
	// immediately. Because the delay is realized on the adapter's (e.g. DB) clock,
	// the absolute target inherits any app-vs-store clock skew — use SendAfter when
	// the delay must be exact.
	SendAt(ctx context.Context, msg Message[T], t time.Time) error
}

// ProducerOption configures NewProducer.
type ProducerOption[T any] func(*producerConfig[T])

type producerConfig[T any] struct {
	codec    PayloadCodec[T]
	codecSet bool
	clock    clockwork.Clock
}

// WithProducerCodec sets the payload codec for a wire adapter (default JSON).
func WithProducerCodec[T any](c PayloadCodec[T]) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.codec = c; o.codecSet = true }
}

// WithProducerClock injects the clock Producer.SendAt uses to convert an absolute
// delivery time into a relative delay. Defaults to a real clock; a nil clock is
// ignored (keeps the default). Named distinctly from the message-level WithClock
// to avoid collision (cf. WithConsumerClock, ADR 0007 D10).
func WithProducerClock[T any](c clockwork.Clock) ProducerOption[T] {
	return func(o *producerConfig[T]) {
		if c != nil {
			o.clock = c
		}
	}
}

type producer[T any] struct {
	out       OutboundAdapter
	codec     PayloadCodec[T]
	liveValue bool
	clock     clockwork.Clock
}

// NewProducer builds a Producer, validating codec pairing at construction.
func NewProducer[T any](out OutboundAdapter, opts ...ProducerOption[T]) (Producer[T], error) {
	if out == nil {
		return nil, ErrNilAdapter
	}
	cfg := producerConfig[T]{clock: clockwork.NewRealClock()}
	for _, opt := range opts {
		opt(&cfg)
	}
	codec, live, err := resolveCodec[T](out, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
	}
	return &producer[T]{out: out, codec: codec, liveValue: live, clock: cfg.clock}, nil
}

// box lifts msg from Message[T] to Message[any]: live-value adapters keep the
// payload unencoded, wire adapters get it encoded to []byte via the codec.
func (p *producer[T]) box(msg Message[T]) (Message[any], error) {
	if p.liveValue {
		return Message[any]{payload: msg.payload, headers: msg.headers}, nil
	}
	b, err := p.codec.Encode(msg.payload)
	if err != nil {
		return Message[any]{}, fmt.Errorf("msgin: producer encode failed: %w", err)
	}
	return Message[any]{payload: any(b), headers: msg.headers}, nil
}

// Send writes msg to the outbound adapter for immediate delivery.
func (p *producer[T]) Send(ctx context.Context, msg Message[T]) error {
	boxed, err := p.box(msg)
	if err != nil {
		return err
	}
	return p.out.Send(ctx, boxed)
}

// SendAfter writes msg for delivery after delay, if the sink supports scheduling.
func (p *producer[T]) SendAfter(ctx context.Context, msg Message[T], delay time.Duration) error {
	sched, ok := p.out.(ScheduledSender)
	if !ok {
		return ErrScheduledSendUnsupported
	}
	if delay < 0 {
		delay = 0
	}
	boxed, err := p.box(msg)
	if err != nil {
		return err
	}
	return sched.SendAfter(ctx, boxed, delay)
}

// SendAt writes msg for delivery no earlier than t (sugar over SendAfter).
func (p *producer[T]) SendAt(ctx context.Context, msg Message[T], t time.Time) error {
	return p.SendAfter(ctx, msg, t.Sub(p.clock.Now()))
}

// isLiveValue reports whether an adapter emits/consumes live Go values.
func isLiveValue(a any) bool {
	lv, ok := a.(LiveValueSource)
	return ok && lv.EmitsLiveValue()
}

// resolveCodec applies the codec-pairing rules shared by NewProducer and
// NewConsumer: a live-value adapter must not be given an explicit codec
// (ErrUnexpectedCodec); a wire adapter with no codec defaults to JSON — this
// JSON default is a deliberate, documented convenience (matching the
// WithConsumerCodec/WithProducerCodec "default JSON" contract), NOT an error, so
// there is no ErrNoPayloadCodec (ADR 0009 D4). A future adapter that needs
// require-codec semantics would signal it via a capability (e.g. RequiresCodec)
// plus a returning error path, added with that adapter. It returns the resolved
// codec and whether adapter is a live-value adapter.
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
