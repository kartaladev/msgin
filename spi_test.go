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
