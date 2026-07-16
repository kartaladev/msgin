package msgin

import (
	"context"
	"fmt"
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
}

// defaultDeliveryBuffer sizes the hand-off channel between the source's
// Stream loop and the worker pool: bounded hand-off; a configurable buffer
// arrives with flow-control (Plan 003).
const defaultDeliveryBuffer = 1

// NewConsumer validates the source and options, and builds a Consumer.
func NewConsumer[T any](src any, h Handler[T], opts ...ConsumerOption[T]) (Consumer[T], error) {
	if src == nil {
		return nil, ErrNilAdapter
	}
	cfg := consumerConfig[T]{concurrency: 1}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.concurrency < 1 {
		return nil, ErrInvalidConcurrency
	}
	codec, live, err := resolveCodec[T](src, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
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
		workers: cfg.concurrency,
	}, nil
}

// Run starts the worker pool, then blocks streaming deliveries from the
// source until ctx is cancelled, draining in-flight deliveries before it
// returns. Shutdown is bounded only if the handler and the settle closures
// (Ack/Nack) honor context cancellation; a handler-timeout arrives with
// flow-control in Plan 003.
func (c *consumer[T]) Run(ctx context.Context) error {
	deliveries := make(chan Delivery, defaultDeliveryBuffer)

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
		// Plan 002: route to invalid-message channel; surface settle
		// failures via the injected logger/error channel.
		_ = d.Nack(ctx, true, 0)
		return
	}
	msg := Message[T]{payload: payload, headers: d.Msg.headers}
	if herr := c.safeHandle(ctx, msg); herr != nil {
		// Plan 002: retry/backoff/DLQ; surface settle failures via the
		// injected logger/error channel.
		_ = d.Nack(ctx, true, 0)
		return
	}
	// Plan 002: surface settle failures via the injected logger/error channel.
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
		return zero, fmt.Errorf("%w: %v", ErrPayloadDecode, err)
	}
	return v, nil
}

// safeHandle recovers a panicking handler so a fault in application code
// never takes down the flow (fault isolation, CLAUDE.md's mandatory
// robustness constraint). The recovered value is reported as a local error;
// dispatch only checks it against nil before Nack'ing, so a plain fmt error
// is sufficient here. A public panic sentinel + observability hook are
// deferred to Plan 002.
func (c *consumer[T]) safeHandle(ctx context.Context, msg Message[T]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("msgin: handler panicked: %v", r)
		}
	}()
	return c.handler(ctx, msg)
}
